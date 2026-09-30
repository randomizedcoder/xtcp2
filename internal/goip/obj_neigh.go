package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// proxyKeywordCst is `ip neigh show proxy`'s keyword. Like devKeywordCst it
// is compared with == because iproute2 compares it with strcmp
// (ip/ipneigh.c:571), so it does not abbreviate; unlike devKeywordCst it is
// spelled in exactly one place, and it is a constant only so that the
// comparison and the reason for its strictness stay together.
const proxyKeywordCst = "proxy"

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

	// NdmFlags is filter.ndm_flags, which `proxy` assigns NTF_PROXY (:572)
	// and nothing else in goip sets. It needs no companion bool: iproute2
	// initializes the field to 0 and writes it unconditionally (:490), so
	// "unset" and "zero" are the same request byte.
	NdmFlags uint8
}

// parseNeighShowArgs walks the selectors after the verb.
//
// # Two keywords, both compared the strict way, and they disagree about repeats
//
// Both are strcmp — `dev` at :526 and `proxy` at :571 — so neither
// abbreviates: `ip neigh show d eth0` is not a device filter and `ip neigh
// show prox` is not the proxy table. What differs is what a second one does.
//
// `dev` is duparg (:528-529) and an ERROR on the second occurrence. Unlike
// route, where `dev` and `oif` are synonyms assigning one variable and the
// last one wins. Three commands in goip now take a `dev` and they disagree
// about repetition; reproducing that is the difference between parsing
// iproute2 and parsing something that resembles it.
//
// `proxy` has no duparg and takes no value — it is the bare assignment
// `filter.ndm_flags = NTF_PROXY` — so `ip neigh show proxy proxy` is accepted
// and means exactly what one `proxy` means. That is idempotence by accident
// of how it is written rather than by design, but it is observable behavior
// and goip reproduces it rather than tidying it up.
//
// The two compose. do_show_or_flush has one loop and no mutual exclusion, so
// `proxy dev eth0` sets both; see neighShow for why that is not two filters
// over one set.
//
// The remaining selectors — master, vrf, nomaster, unused, nud, protocol, and
// the bare `to PREFIX` else-arm — are rejected rather than ignored, the policy
// every other object in goip follows: answering a filtered query with an
// unfiltered dump is a wrong answer, not a missing feature.
func parseNeighShowArgs(args []string) (neighSelectors, error) {
	var sel neighSelectors
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case proxyKeywordCst:
			// No NEXT_ARG and no duparg at :571-572, so there is nothing to
			// check: a repeat assigns the same bit again. Written as an
			// assignment rather than an |= for the same reason iproute2
			// writes one — see BuildDumpNeighRequestFilter on why the
			// kernel's test is an equality and NTF_PROXY may not be or-ed
			// with anything.
			sel.NdmFlags = unix.NTF_PROXY
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

// neighShowVerbs are the three spellings ip/ipneigh.c:758-760 accepts for the
// listing, each through matches() and so each abbreviable.
//
// `lst` is the one that is easy to miss, because it is not a prefix of
// anything else and reads like a typo. It is a real alias, unique to the neigh
// object — ipaddress.c and iproute.c have "show"/"list"/"lst" too, but goip's
// other objects reach them by different paths. Spelling the set as a slice
// rather than a chain of conditions keeps the next alias a one-line change.
var neighShowVerbs = []string{"show", "list", "lst"}

func runNeigh(c *runCtx, args []string) error {
	if len(args) > 0 && !matchesAny(args[0], neighShowVerbs) {
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
// order, and neither `dev NAME` nor `proxy` adds a third or reorders them.
//
// # The link dump is unconditional, and it is what makes the selector free
//
// do_show_or_flush calls ll_init_map(&rth) at ip/ipneigh.c:597 after the
// argument loop and before anything else, for every form of the command. Only
// then does it resolve `dev` (:600), out of the cache that dump just filled.
// So every form sends the same first request. `dev NAME` differs by the 8
// bytes of NDA_IFINDEX on the second and `proxy` by one byte inside it — where
// `route show dev` moved a get to the front and deleted the lazy ones from the
// back, and `addr show dev` went from two transactions to three.
// req.NeighShowDump carries the full comparison.
//
// # The filtering is done twice, on purpose, as `ip` does it twice
//
// The kernel applies NDA_IFINDEX on the dump side, and print_neigh applies
// `filter.index != r->ndm_ifindex` again on every reply (:331). Keeping both
// matters for the replay source, which answers a filtered request with the
// whole recorded dump: without the client-side test a fixture captured for the
// bare command would render as though the filter had done nothing.
//
// # `proxy` is a different TABLE, not a narrower view of this one
//
// The kernel reads ndm_flags on the request to choose between
// pneigh_dump_table and neigh_dump_table (net/core/neighbour.c:2956), so the
// two commands return disjoint sets. There is deliberately no client-side
// re-check of NTF_PROXY to match the NDA_IFINDEX one below: a replay source
// answering `proxy` from a bare-command fixture would return the wrong table
// entirely, and no amount of filtering turns one into the other. The
// divergence has to be visible, not papered over.
//
// # The state filter, and the escape that exists for exactly this command
//
// filter.state is `0xFF & ~NUD_NOARP` for show (:523), which is why the
// multicast neighbor-cache rows never appear. It is a default rather than a
// constant — `nud STATE` replaces it — so the skip lives here with the other
// filtering rather than in the renderer.
//
// The test at :335-339 is not simply that test, though. It skips a reply when
// the state misses the mask AND the entry is neither NTF_PROXY nor
// NTF_EXT_LEARNED. Proxy entries carry ndm_state 0, which misses every mask,
// so without that escape `ip neigh show proxy` would print nothing at all —
// the two halves of :335-339 are what make the command work. Writing the skip
// as "NUD_NOARP only" would pass the proxy rows through for the wrong reason
// and would silently let a genuine state-0 non-proxy entry through too, so it
// is written out in full.
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

	// Transaction two: the dump. The device narrows it kernel-side; ndm_flags
	// chooses which table the kernel walks in the first place.
	neighbors, err := svc.Neighbors(c.family, sel.NdmFlags, index)
	if err != nil {
		return err
	}

	f := render.NeighShowFilter{IndexSet: index != 0}
	views := make([]render.NeighView, 0, len(neighbors))
	for i := range neighbors {
		n := xtcpnl.NeighInfo(neighbors[i])
		if neighStateFiltered(n) {
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

// neighShowStateMaskCst is filter.state for `show`, set at ip/ipneigh.c:523 as
// `~0 & ~NUD_NOARP`. It is a constant in goip because `nud STATE` — the only
// thing that replaces it (:553-570) — is not implemented; see
// neighStateFiltered for the one clause of iproute2's test that assumption
// removes.
const neighShowStateMaskCst = 0xFF &^ uint16(unix.NUD_NOARP)

// neighStateFiltered is print_neigh's state test (ip/ipneigh.c:335-339),
// reporting whether a reply is dropped before rendering.
//
// iproute2 writes it as one four-clause conjunction that SKIPS when all four
// hold:
//
//	!(filter.state & r->ndm_state) &&
//	!(r->ndm_flags & NTF_PROXY) &&
//	!(r->ndm_flags & NTF_EXT_LEARNED) &&
//	(r->ndm_state || !(filter.state & 0x100))
//
// The first clause is the ordinary state filter, and on its own it is what
// hides the NUD_NOARP multicast rows. The next two are escapes: a proxy or
// externally-learned entry is printed whatever its state, which is not a
// nicety — pneigh entries carry ndm_state 0, so `ip neigh show proxy` would
// print an empty dump without the NTF_PROXY clause.
//
// The fourth clause is absent here, and deliberately. 0x100 is the sentinel
// `nud none` assigns when nud_state_a2n yields 0 (:568-569); it is the only
// way that bit enters filter.state, and goip does not implement `nud`. At the
// fixed neighShowStateMaskCst the clause is therefore constant true and
// contributes nothing. Reinstate it in the same commit that adds `nud`, or
// `ip neigh show nud none` will print state-0 entries goip drops.
func neighStateFiltered(n xtcpnl.NeighInfo) bool {
	if neighShowStateMaskCst&n.State != 0 {
		return false
	}
	return n.Flags&(unix.NTF_PROXY|unix.NTF_EXT_LEARNED) == 0
}
