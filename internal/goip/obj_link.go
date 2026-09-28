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
// That reasoning holds *because* the dump fills the cache. linkShowDev below
// sends no dump, so its cache holds one link and it takes the side-get path on
// purpose — `ip` takes it there too, and for the same reason.
//
// `dev X` uses the two-single-get path in linkShowDev. Other filtering
// arguments (`up`, `group G`, `master M`) are not implemented and are rejected
// rather than ignored.
func linkShow(c *runCtx, args []string) error {
	if len(args) == 2 && matchesPrefix(args[0], "dev") {
		return linkShowDev(c, args[1])
	}
	if len(args) > 0 {
		return fmt.Errorf("link show %q: %w", args[0], ErrNotImplemented)
	}

	resources, err := service.New(c.src, c.nextSeq).Links(c.linkExtMask())
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
		v := render.LinkViewOf(links[i], c.lltab)
		if c.showStats > 0 {
			v = v.WithStats(links[i])
		}
		views = append(views, v)
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

// linkShowDev serves `ip link show dev NAME` as the two by-name single-gets,
// and no dump, that iproute2 sends.
//
// # Why one link costs two requests
//
// ipaddr_list_link resolves the selector and prints the result through paths
// that do not share a reply. `dev NAME` is parsed into filter.ifindex by
// ll_name_to_index (ip/ipaddress.c:2254), which on a cache miss issues
// ll_link_get(name, 0) on a throwaway socket (lib/ll_map.c:264, :354-372)
// purely to learn the index. Then, filter.ifindex being set, iplink_get
// re-fetches that same link — by name again, not by the index just learned —
// on the main socket (:2293), and that second reply is the one print_linkinfo
// renders. The first is discarded apart from the cache entry it leaves behind.
//
// The redundancy is iproute2's, and goip reproduces it rather than improving
// on it: pkg/nlparity compares the two tools' requests for byte equality, so
// sending one request where `ip` sends two is an L1 transaction-count
// divergence before any attribute is even looked at.
//
// # Why not dump, resolve locally, then get by index
//
// That is the shape this function replaced, and it is instructive: it keeps
// the transaction count at two, so it passes L1, and it prints the same bytes,
// so stdout parity is clean — while both of its requests are wrong at L2. A
// dump carries NLM_F_DUMP, AF_PACKET and no IFLA_IFNAME; a by-index get
// carries ifi_index where iproute2 carries a name. Only full request equality
// catches it, which is why that assertion exists.
//
// It was also over-generous with the cache: dumping filled lltab with every
// link on the box, so any later name lookup this command made would hit in
// memory and send nothing, hiding precisely the side-gets the harness is built
// to observe. Here only the link that was asked for is cached, which is all
// `ip` has at print time too.
func linkShowDev(c *runCtx, name string) error {
	svc := service.New(c.src, c.nextSeq)

	// First get: ll_name_to_index. Nothing is rendered from this reply — it is
	// sent for the cache entry, exactly as in iproute2.
	resolved, err := svc.LinkByName(name)
	if err != nil {
		return err
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(resolved)})

	// Second get: iplink_get. This reply is the one that gets printed, and
	// the only one of the two that `-s` changes — ll_link_get above builds
	// its mask as a local constant (lib/ll_map.c:276-277).
	shown, err := svc.LinkShowDev(name, c.linkExtMask())
	if err != nil {
		return err
	}
	link := xtcpnl.LinkInfo(shown)
	c.lltab.Fill([]xtcpnl.LinkInfo{link})
	resolveLinkRefs(c, svc, link)

	view := render.LinkViewOf(link, c.lltab)
	if c.showStats > 0 {
		view = view.WithStats(link)
	}
	if c.json {
		return json.NewEncoder(c.out).Encode([]render.LinkView{view})
	}
	_, err = fmt.Fprint(c.out, view.Text())
	return err
}

// resolveLinkRefs sends the side-gets print_linkinfo issues for the two
// indexes a link stanza can name besides its own.
//
// Both go through ll_index_to_name, so both are lazy in exactly the sense
// resolveIndexName implements: nothing is sent for index 0 or for an index the
// cache already holds. The order is print_linkinfo's own —
// print_name_and_link at ip/ipaddress.c:1018 runs before the IFLA_MASTER block
// at :1032 — and it is observable, because two gets on one socket appear in a
// capture in the order they were sent.
//
// The IFLA_LINK case is conditional in a way the master case is not.
// print_name_and_link resolves the peer only when IFLA_LINK_NETNSID is absent
// (lib/utils.c:1310-1320); with a netnsid present it calls ll_idx_n2a, which
// is an unconditional "if%u" — no cache, no netlink, not even a lookup. Every
// veth in the committed 7_1_8 dump carries a netnsid, which is why
// ip_link_n shows `ve-nfb-vpn@if2` and why `link show dev` on one of them
// sends two requests rather than three.
func resolveLinkRefs(c *runCtx, svc *service.Service, li xtcpnl.LinkInfo) {
	if li.Link != 0 && !li.HasLinkNetnsID {
		resolveIndexName(c, svc, li.Link)
	}
	resolveIndexName(c, svc, li.Master)
}
