package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
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
// Filtering arguments (`dev X`, `up`, `group G`, `master M`) are not
// implemented. They are not cosmetic to skip — `dev X` changes the request
// from a dump to a single-get — so they are rejected rather than ignored,
// because silently dumping everything in response to `link show dev lo` would
// be a wrong answer rather than a missing feature.
func linkShow(c *runCtx, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("link show %q: %w", args[0], ErrNotImplemented)
	}

	request, err := req.LinkShowDump(c.nextSeq())
	if err != nil {
		return fmt.Errorf("goip: build link dump request: %w", err)
	}

	bodies, err := c.src.Dump(request, uint16(unix.RTM_NEWLINK))
	if err != nil {
		return err
	}

	links := make([]xtcpnl.LinkInfo, 0, len(bodies))
	for _, body := range bodies {
		li, perr := xtcpnl.ParseNewLink(body)
		if perr != nil {
			return fmt.Errorf("goip: decode RTM_NEWLINK: %w", perr)
		}
		links = append(links, li)
	}
	c.lltab.Fill(links)

	// Ranged by index: LinkInfo is 176 bytes and LinkView 280, so the value form
	// of either loop copies the whole struct once per link for nothing.
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

// runRoute and runNeigh are placeholders so the object table can be complete
// and its abbreviation behavior testable before the handlers exist. They
// report ErrNotImplemented, which the dispatch tests distinguish from
// ErrUnknownObject.
func runRoute(_ *runCtx, _ []string) error {
	return fmt.Errorf("route: %w", ErrNotImplemented)
}

func runNeigh(_ *runCtx, _ []string) error {
	return fmt.Errorf("neigh: %w", ErrNotImplemented)
}
