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

// nudKeywordCst is `ip neigh show nud STATE`'s keyword, strcmp at
// ip/ipneigh.c:553 like `dev` and `proxy`, so `nu` is not it.
const nudKeywordCst = "nud"

// nudAllCst is the one state word nud_state_a2n does not know: do_show_or_flush
// tests for it with strcmp only after nud_state_a2n has failed (:561-562, :564).
const nudAllCst = "all"

// nudNoneSentinelCst is the bit `nud none` (or `nud 0`) adds to filter.state
// (:568-570). NUD_NONE is 0, which an or cannot record, so iproute2 borrows the
// first bit above ndm_state's 8 NUD bits; print_neigh's fourth clause reads it
// back (:338).
const nudNoneSentinelCst uint32 = 0x100

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

	// State is filter.state: neighShowStateMaskCst (:523) until the first
	// `nud`, which zeroes it (:557-560) before or-ing its own state in, so
	// every later `nud` adds to the first rather than to the default. A uint32
	// because `all` or-s in ~0 (:564), and filter.state is an int whose high
	// bits print_neigh's tests then see; only the low 9 bits are ever read.
	State uint32

	// StateGiven is do_show_or_flush's local state_given (:509): whether a
	// `nud` has replaced the default yet.
	StateGiven bool
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
// # `nud`, the third strict keyword, and the only one that accumulates
//
// `nud STATE` (:553-570) is strcmp too, and NEXT_ARG takes its value, so a
// trailing `nud` is refused as a trailing `dev` is. Its first occurrence
// zeroes the default mask and every occurrence ors one state in, so `nud stale
// nud failed` is both and `nud stale` alone drops the PERMANENT rows the
// default shows. The value goes through nud_state_a2n (:61-88): three words abbreviate
// (permanent, reachable and failed are matches()), six are exact, and a
// number is get_unsigned base 0 that must be a single NUD bit or 0, below
// 0x100. `all` is accepted only after that fails, and ors in ~0. A state of
// 0 (`none`, `0`, `0x0`) becomes nudNoneSentinelCst (:568-569). Anything else
// is invarg — `Error: argument "X" is wrong: nud state is bad` and exit 255 —
// which goip wraps in ErrInvalidArgument (ExitUsage, an HTTP 400), as it
// does a bad `dev`.
//
// The remaining selectors — master, vrf, nomaster, unused, protocol, and
// the bare `to PREFIX` else-arm — are rejected rather than ignored, the policy
// every other object in goip follows: answering a filtered query with an
// unfiltered dump is a wrong answer, not a missing feature.
func parseNeighShowArgs(args []string) (neighSelectors, error) {
	sel := neighSelectors{State: neighShowStateMaskCst}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case nudKeywordCst:
			if i+1 >= len(args) {
				return sel, fmt.Errorf("%w: neigh show: argument %q is missing its value", ErrInvalidArgument, args[i])
			}
			i++
			if !sel.StateGiven {
				sel.StateGiven, sel.State = true, 0
			}
			state, ok := nudStateA2n(args[i])
			if !ok {
				if args[i] != nudAllCst {
					return sel, fmt.Errorf("%w: neigh show: argument %q is wrong: nud state is bad", ErrInvalidArgument, args[i])
				}
				state = ^uint32(0)
			}
			if state == 0 {
				state = nudNoneSentinelCst
			}
			sel.State |= state
		case proxyKeywordCst:
			// No NEXT_ARG and no duparg at :571-572, so there is nothing to
			// check: a repeat assigns the same bit again. Written as an
			// assignment rather than an |= for the same reason iproute2
			// writes one — see BuildDumpNeighRequestFilter on why the
			// kernel's test is an equality and NTF_PROXY may not be or-ed
			// with anything.
			sel.NdmFlags = unix.NTF_PROXY
		case devKeywordCst:
			// The two errors below wrap ErrInvalidArgument, as route's bad
			// `table` and `proto` do, so goiphttp answers them with a 400
			// rather than a 500 and the CLI exits ExitUsage. `ip` exits 255
			// on both — NEXT_ARG() prints "Command line is not complete"
			// and duparg prints `Duplicate "dev": "..." is the second
			// value.` — which neither goip code matches.
			if i+1 >= len(args) {
				return sel, fmt.Errorf("%w: neigh show: argument %q is missing its value", ErrInvalidArgument, args[i])
			}
			if sel.DevSet {
				return sel, fmt.Errorf("%w: neigh show: duplicate %q argument", ErrInvalidArgument, devKeywordCst)
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
	args, err := showArgs("neigh", neighVerbs, args)
	if err != nil {
		return err
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
// constant — `nud STATE` replaces it, and adds nothing to either request,
// because ipneigh_dump_filter writes only ndm_flags, NDA_IFINDEX and
// NDA_MASTER (:485-503) — so the skip lives here with the other filtering
// rather than in the renderer or the kernel.
//
// The test at :335-339 is not simply that test, though. It skips a reply when
// the state misses the mask AND the entry is neither NTF_PROXY nor
// NTF_EXT_LEARNED. Proxy entries carry ndm_state 0, which misses every mask,
// so without that escape `ip neigh show proxy` would print nothing at all —
// the two halves of :335-339 are what make the command work. Writing the skip
// as "NUD_NOARP only" would pass the proxy rows through for the wrong reason
// and would silently let a genuine state-0 non-proxy entry through too, so it
// is written the way iproute2 writes it — the mask test and both flag escapes
// in one condition. See neighStateFiltered.
//
// # `-d` reaches nothing here, and that is a property worth asserting
//
// This function takes no show_details and passes none on, because ip/ipneigh.c
// does not contain the identifier. `ip neigh show` is the only object goip
// implements whose output -d leaves byte-identical, and the committed pair says
// so with no diff at all: ip_neigh and ip_neigh_n are the same file.
//
// It is a real coverage row rather than a triviality, because "-d changed
// nothing" and "-d was dropped on the floor" produce the same output for this
// command and different output for the other three. See
// TestNeighShowIgnoresDetails, which asserts the equality against the same
// invocation that proves -d is wired up at all.
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
			return noSuchDevice(fmt.Errorf("neigh show: cannot find device %q", sel.Dev))
		}
		index = uint32(idx)
	}

	// Transaction two: the dump. The device narrows it kernel-side; ndm_flags
	// chooses which table the kernel walks in the first place.
	neighbors, err := svc.Neighbors(c.family, sel.NdmFlags, index)
	if err != nil {
		return err
	}

	f := render.NeighShowFilter{IndexSet: index != 0, Stats: c.stats()}
	views := make([]render.NeighView, 0, len(neighbors))
	for i := range neighbors {
		n := xtcpnl.NeighInfo(neighbors[i])
		if neighStateFiltered(n, sel.State) {
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

// neighShowStateMaskCst is filter.state's default for `show`, set at
// ip/ipneigh.c:523 as `0xFF & ~NUD_NOARP`. The first `nud STATE` replaces it
// (:557-560); see parseNeighShowArgs.
const neighShowStateMaskCst = 0xFF &^ uint32(unix.NUD_NOARP)

// nudStateA2n is nud_state_a2n (ip/ipneigh.c:61-88), returning false where it
// returns -1. The order matters for the three matches() words: `p` is
// permanent, tested before the exact `probe`, and `f` is failed.
func nudStateA2n(arg string) (uint32, bool) {
	switch {
	case matchesPrefix(arg, "permanent"):
		return unix.NUD_PERMANENT, true
	case matchesPrefix(arg, "reachable"):
		return unix.NUD_REACHABLE, true
	case arg == "noarp":
		return unix.NUD_NOARP, true
	case arg == "none":
		return unix.NUD_NONE, true
	case arg == "stale":
		return unix.NUD_STALE, true
	case arg == "incomplete":
		return unix.NUD_INCOMPLETE, true
	case arg == "delay":
		return unix.NUD_DELAY, true
	case arg == "probe":
		return unix.NUD_PROBE, true
	case matchesPrefix(arg, "failed"):
		return unix.NUD_FAILED, true
	}
	// get_unsigned base 0, then `*state >= 0x100 || (*state&((*state)-1))`:
	// one NUD bit, or 0.
	v, ok := getU32(arg, 0)
	if !ok || v >= nudNoneSentinelCst || v&(v-1) != 0 {
		return 0, false
	}
	return v, true
}

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
// The fourth clause is the `nud none` escape. nudNoneSentinelCst is the bit
// `nud none` (or `all`) puts in filter.state (:568-570), and a state-0 entry
// survives exactly when it is set, so `nud none` lists the entries with no
// state and the default mask, which lacks the bit, drops them.
func neighStateFiltered(n xtcpnl.NeighInfo, state uint32) bool {
	if state&uint32(n.State) != 0 {
		return false
	}
	if n.Flags&(unix.NTF_PROXY|unix.NTF_EXT_LEARNED) != 0 {
		return false
	}
	return n.State != 0 || state&nudNoneSentinelCst == 0
}
