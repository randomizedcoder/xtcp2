package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// runLink handles `ip link ...`. Only the show verbs are implemented.
//
// iproute2 accepts "show", "lst" and "list" here, all through matches(), so
// `ip link s`, `ip link sh` and `ip link l` all work (ip/iplink.c:1834-1837).
// A bare `ip link` with no verb is also a show — do_iplink falls through to
// ipaddr_list_link when argc is 0.
func runLink(c *runCtx, args []string) error {
	if len(args) == 0 {
		return linkShow(c, nil)
	}
	verb := args[0]
	switch {
	case matchesPrefix(verb, "show"), matchesPrefix(verb, "lst"), matchesPrefix(verb, "list"):
		return linkShow(c, args[1:])
	default:
		return fmt.Errorf("link %q: %w", verb, ErrNotImplemented)
	}
}

// linkShow runs the dump and renders it.
//
// # Exactly one netlink transaction, and the argument order that guarantees it
//
// The dump is sent first and the index cache is filled from its own replies,
// before any rendering happens. That ordering is the whole reason `link show`
// emits a single transaction: rendering `master br-3a5828b2963a` or a `@peer`
// suffix needs an index resolved, and iproute2's ll_index_to_name will issue a
// live RTM_GETLINK single-get for an index it does not know
// (lib/ll_map.c:320). Filling first means every index the replies reference is
// already cached, so goip never takes that path — which matters because a side
// transaction inside a capture window is an L1 transaction-count divergence,
// the harness's highest-value assertion.
//
// `dev X` uses the single-get path. Other filtering arguments (`up`, `group
// G`, `master M`) are not implemented and are rejected rather than ignored.
func linkShow(c *runCtx, args []string) error {
	if len(args) == 2 && matchesPrefix(args[0], "dev") {
		svc := service.New(c.src, c.nextSeq)
		resources, err := svc.Links()
		if err != nil {
			return err
		}
		cacheLinks := make([]xtcpnl.LinkInfo, len(resources))
		for i := range resources {
			cacheLinks[i] = xtcpnl.LinkInfo(resources[i])
		}
		c.lltab.Fill(cacheLinks)
		index := c.lltab.NameToIndex(args[1])
		if index == 0 {
			return fmt.Errorf("goip: link %q not found", args[1])
		}
		resource, err := svc.LinkByIndex(index)
		if err != nil {
			return err
		}
		links := []xtcpnl.LinkInfo{xtcpnl.LinkInfo(resource)}
		c.lltab.Fill(links)
		view := render.LinkViewOf(links[0], c.lltab)
		if c.json {
			return json.NewEncoder(c.out).Encode([]render.LinkView{view})
		}
		_, err = fmt.Fprint(c.out, view.Text())
		return err
	}
	if len(args) > 0 {
		return fmt.Errorf("link show %q: %w", args[0], ErrNotImplemented)
	}

	resources, err := service.New(c.src, c.nextSeq).Links()
	if err != nil {
		return err
	}
	links := make([]xtcpnl.LinkInfo, len(resources))
	for i := range resources {
		links[i] = xtcpnl.LinkInfo(resources[i])
	}
	c.lltab.Fill(links)

	// Ranged by index: both structs are deliberately large, so the value form
	// of either loop would copy the whole resource once per link for nothing.
	views := make([]render.LinkView, 0, len(links))
	for i := range links {
		views = append(views, render.LinkViewOf(links[i], c.lltab))
	}

	if c.json {
		enc := json.NewEncoder(c.out)
		return enc.Encode(views)
	}
	for i := range views {
		if _, werr := fmt.Fprint(c.out, views[i].Text()); werr != nil {
			return werr
		}
	}
	return nil
}
