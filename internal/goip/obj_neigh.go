package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// neighSelectors is the subset of do_show_or_flush's filter struct that goip
// can set (ip/ipneigh.c:506-594).
type neighSelectors struct {
	// Dev is filter_dev, the pending `dev NAME`. It is resolved to
	// filter.index after the loop and after ll_init_map, not inside it.
	Dev string

	// DevSet is `filter_dev != NULL`, kept separate from Dev != "" for the
	// same reason routeSelectors.DevSet is: `ip neigh show dev ""` is a real
	// command that resolves nothing and exits `Cannot find device ""`.
	DevSet bool
}

// parseNeighShowArgs walks the selectors after the verb.
//
// # One keyword, compared the strict way, and it can be given only once
//
// `dev` is compared with strcmp (:524), so it does not abbreviate — `ip neigh
// show d eth0` is not a device filter. Unlike route, where `dev` and `oif` are
// synonyms assigning one variable and the last one wins, a second `dev` here
// is duparg (:526-527) and an ERROR. Three commands in goip now take a `dev`
// and they disagree about repetition; reproducing that is the difference
// between parsing iproute2 and parsing something that resembles it.
//
// The remaining selectors — master, vrf, nomaster, unused, nud, proxy,
// protocol, and the bare `to PREFIX` else-arm — are rejected rather than
// ignored, the policy every other object in goip follows: answering a filtered
// query with an unfiltered dump is a wrong answer, not a missing feature.
//
// `proxy` is the nearest of those and is deliberately still refused. It is one
// byte, ndm_flags = NTF_PROXY (:570), but it is a byte on the REQUEST, so
// accepting it without setting it would make goip send `neigh show`'s bytes
// for a different command — precisely the divergence the parity comparator
// exists to catch, produced on purpose.
func parseNeighShowArgs(args []string) (neighSelectors, error) {
	var sel neighSelectors
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case devKeywordCst:
			// The two errors below are plain, with no sentinel, and so map
			// to ExitFailure rather than ExitUsage (goip.go:140-146). That
			// is the correct half of the distinction: goip implements `dev`,
			// so a malformed `dev` is a bad command line, not a missing
			// feature. `ip` exits nonzero on both as well — NEXT_ARG() prints
			// "Command line is not complete" and duparg prints
			// `Duplicate "dev": "..." is the second value.`
			if i+1 >= len(args) {
				return sel, fmt.Errorf("neigh show: argument %q is missing its value", args[i])
			}
			if sel.DevSet {
				return sel, fmt.Errorf("neigh show: duplicate %q argument", devKeywordCst)
			}
			i++
			sel.Dev, sel.DevSet = args[i], true
		default:
			return sel, fmt.Errorf("neigh show %q: %w", args[i], ErrNotImplemented)
		}
	}
	return sel, nil
}

func runNeigh(c *runCtx, args []string) error {
	if len(args) > 0 && !matchesPrefix(args[0], "show") && !matchesPrefix(args[0], "list") {
		return fmt.Errorf("neigh %q: %w", args[0], ErrNotImplemented)
	}
	if len(args) > 0 {
		args = args[1:]
	}
	sel, err := parseNeighShowArgs(args)
	if err != nil {
		return err
	}
	return neighShow(c, sel)
}

// neighShow sends the two transactions `ip neigh show` sends, in iproute2's
// order, and `dev NAME` adds neither a third nor reorders them.
//
// # The link dump is unconditional, and it is what makes the selector free
//
// do_show_or_flush calls ll_init_map(&rth) at ip/ipneigh.c:597 after the
// argument loop and before anything else, for every form of the command. Only
// then does it resolve `dev` (:600), out of the cache that dump just filled.
// So the two commands send the same first request and differ by the 8 bytes of
// NDA_IFINDEX on the second — where `route show dev` moved a get to the front
// and deleted the lazy ones from the back, and `addr show dev` went from two
// transactions to three. req.NeighShowDump carries the full comparison.
//
// # The filtering is done twice, on purpose, as `ip` does it twice
//
// The kernel applies NDA_IFINDEX on the dump side, and print_neigh applies
// `filter.index != r->ndm_ifindex` again on every reply (:331). Keeping both
// matters for the replay source, which answers a filtered request with the
// whole recorded dump: without the client-side test a fixture captured for the
// bare command would render as though the filter had done nothing.
//
// # NUD_NOARP is dropped before rendering and that is the default filter
//
// filter.state is `0xFF & ~NUD_NOARP` for show (:522), which is why the
// multicast neighbor-cache rows never appear. It is a default rather than a
// constant — `nud STATE` replaces it — so the skip lives here with the other
// filtering rather than in the renderer.
func neighShow(c *runCtx, sel neighSelectors) error {
	svc := service.New(c.src, c.nextSeq)

	// Transaction one: ll_init_map. Its replies are the index cache, and for
	// `dev NAME` they are also the name resolution.
	links, err := svc.NeighborLinks()
	if err != nil {
		return err
	}
	decodedLinks := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decodedLinks[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decodedLinks)

	var index uint32
	if sel.DevSet {
		// ll_name_to_index, answered from the cache (lib/ll_map.c:354-359).
		// A miss would send ll_link_get in `ip`; goip reports the failure
		// instead, the same position routeShow takes and for the same reason.
		idx := c.lltab.NameToIndex(sel.Dev)
		if idx == 0 {
			return fmt.Errorf("neigh show: cannot find device %q", sel.Dev)
		}
		index = uint32(idx)
	}

	// Transaction two: the neighbor dump, filtered by the kernel when a device
	// was named.
	neighbors, err := svc.Neighbors(c.family, index)
	if err != nil {
		return err
	}

	f := render.NeighShowFilter{IndexSet: index != 0}
	views := make([]render.NeighView, 0, len(neighbors))
	for i := range neighbors {
		n := xtcpnl.NeighInfo(neighbors[i])
		if n.State&unix.NUD_NOARP != 0 {
			continue
		}
		if index != 0 && uint32(n.Ifindex) != index {
			continue
		}
		views = append(views, render.NeighViewOf(n, c.lltab, f))
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
