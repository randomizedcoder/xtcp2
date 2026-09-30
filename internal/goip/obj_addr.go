package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runAddress handles `ip address ...`. Only the show verbs are implemented.
//
// iproute2's verb list for this object is longer than link's and, because
// every entry goes through matches(), the abbreviations interact. do_ipaddr
// tests in this order: add, change (plus the exact-match alias "chg"),
// replace, delete, list|show|lst, flush, save, showdump, restore, help.
//
// So `ip a s` is **show**, because show is tested before save; `ip a sa` is
// **save**, because "sa" is not a prefix of "show"; and `ip a r` is
// **replace**, not restore. goip resolves the first of those the same way and
// refuses the other two, which is the useful distinction: an argument that
// means a write in `ip` must not mean a read in goip.
//
// The five write verbs are named rather than left to the default so the
// refusal says why. The default arm covers save/showdump/restore, which are
// read-only but out of scope.
func runAddress(c *runCtx, args []string) error {
	if len(args) == 0 {
		return addrShow(c, nil)
	}
	verb := args[0]
	switch {
	case matchesPrefix(verb, "show"), matchesPrefix(verb, "lst"), matchesPrefix(verb, "list"):
		return addrShow(c, args[1:])
	case matchesPrefix(verb, "add"), matchesPrefix(verb, "change"),
		matchesPrefix(verb, "replace"), matchesPrefix(verb, "delete"),
		matchesPrefix(verb, "flush"):
		// Named explicitly, and refused for a stronger reason than "not
		// written yet": pkg/xtcpnl.BuildRequest rejects every msgType
		// outside RTM_GET*, so there is no code path from here to an
		// RTM_NEWADDR even if a future edit wired one up.
		return fmt.Errorf("address %q: goip is read-only: %w", verb, ErrNotImplemented)
	default:
		return fmt.Errorf("address %q: %w", verb, ErrNotImplemented)
	}
}

// addrShow dispatches `dev NAME` to addrShowDev and otherwise runs the two
// dumps and renders them.
//
// # Two transactions, in this order, and the order is the assertion
//
// This is the first command in goip that sends more than one request, which
// makes it the first real test of the parity harness's L1 finding — positional
// transaction count. iproute2 sends the link dump first
// (`ip_link_list`, ip/ipaddress.c:2306) and the address dump second
// (`ip_addr_list`, :2314), and it needs both: the output is a loop over
// *links*, with each link's addresses filtered out of the address list
// (:2320-2334). A goip that dumped addresses only, or that dumped them in the
// other order, would produce plausible output and diverge on the wire.
//
// The second dump is skipped entirely when the family is AF_PACKET (:2310).
// That arm is reached by `-0`, which goip parses (goip.go:61) — this comment
// used to say it was unreachable, which was true only until the option
// landed. See the family argument threading through req.AddrShowLinkDump,
// where AF_PACKET selects a different request shape too.
//
// # Why the index cache is filled between the dumps and not after
//
// Same reason as link show: rendering `master br0` resolves an index, and an
// unresolved index sends iproute2 off to do a live single-get. Filling from
// the link dump's own replies means the cache is complete before any
// rendering, so goip emits exactly the two transactions and no side traffic.
// Here it matters more than for link show, because the address dump arrives
// between the fill and the render and it would be easy to render as replies
// are decoded.
//
// # What is accepted here, and the one form that is deliberately not
//
// `dev NAME` uses the three-request path in addrShowDev. The keyword is
// compared with `==` and not matchesPrefix, because `ip` compares it with
// strcmp (:2241) — `d` is not an abbreviation of `dev` there, it is a device
// NAME, and accepting as a keyword what `ip` reads as a name would be a
// divergence in argument parsing rather than a convenience.
//
// The other filtering arguments (`scope S`, `to PREFIX`, `up`, `label L`,
// `master M`, `primary`, `secondary`, `tentative`, `deprecated`, `dynamic`,
// `permanent`) are rejected rather than ignored, for the reason linkShow
// gives: answering a filtered query with an unfiltered dump is a wrong answer
// rather than a missing feature.
//
// And so is the BARE device name, which `ip addr show lo` accepts through the
// same else-arm `dev` falls into. That arm is reached only after every keyword
// test above it has failed, so implementing it while those keywords are
// unimplemented would turn `goip addr show up` from an honest refusal into
// `Device "up" does not exist` — a wrong answer produced by a feature, which
// is worse than a missing one.
func addrShow(c *runCtx, args []string) error {
	if len(args) == 2 && args[0] == devKeywordCst {
		return addrShowDev(c, args[1])
	}
	if len(args) > 0 {
		return fmt.Errorf("address show %q: %w", args[0], ErrNotImplemented)
	}

	linkResources, addrResources, err := service.New(c.src, c.nextSeq).AddressSnapshot(c.family)
	if err != nil {
		return err
	}
	links := make([]xtcpnl.LinkInfo, len(linkResources))
	for i := range linkResources {
		links[i] = xtcpnl.LinkInfo(linkResources[i])
	}
	c.lltab.Fill(links)

	var addrs []xtcpnl.AddrInfo
	if c.family != unix.AF_PACKET {
		addrs = make([]xtcpnl.AddrInfo, len(addrResources))
		for i := range addrResources {
			addrs[i] = xtcpnl.AddrInfo(addrResources[i])
		}
	}
	return renderAddrGroups(c, links, addrs)
}

// addrShowDev serves `ip addr show dev NAME` as the three transactions
// iproute2 sends, in iproute2's order.
//
// # Three requests, and the middle one is not the one you would guess
//
// `dev NAME` lands in the same catch-all else-arm every `addr` filter argument
// falls through to (ip/ipaddress.c:2241-2247) and is resolved by
// ll_name_to_index at :2253. On a cache miss that issues ll_link_get(name, 0)
// on a throwaway socket, exactly as `link show dev` does — request one, its
// reply thrown away apart from the cache entry.
//
// filter.ifindex is now set, so :2302 takes the ipaddr_link_get branch instead
// of ip_link_list's dump: request two is a single-get addressed BY THE INDEX
// just learned, not by the name. That is the one place the two `dev` commands
// diverge — `link show dev` re-fetches by name through iplink_get (:2293),
// this one re-fetches by index — and the difference is entirely in request
// bytes, so only pkg/nlparity's byte comparison can see it.
//
// Request three is the address dump, with the index written into ifa_index by
// ipaddr_dump_filter (:1954-1958). So the kernel does the address filtering
// here where `addr show` does it client-side, and addrBelongsTo below runs
// anyway, because `ip` applies both too.
//
// # The side-gets come after the dump, and the order is observable
//
// print_linkinfo runs in the loop at :2323-2336, which is after ip_addr_list
// at :2314. So a link with an IFLA_MASTER or an unshadowed IFLA_LINK emits its
// lazy ll_index_to_name single-gets as transactions FOUR and later, behind the
// address dump — where linkShowDev, having no dump to send, emits them second.
// Same function, same laziness, different position in the capture, and the
// harness compares positionally. Hence resolveLinkRefs is called here after
// Addresses returns and not before.
//
// The gated clean topology has neither a master nor a peer, so it sends
// exactly three; the mesh namespace is where the fourth would appear.
//
// # What `-s` would add, and why it is not here
//
// `-s` reaches this command in `ip` twice over: ipaddr_link_get clears
// RTEXT_FILTER_SKIP_STATS from its mask (:2066-2067), and the print loop calls
// print_link_stats at :2333 under `!do_link && show_stats`. goip implements
// neither, and the two are one item rather than two — the mask alone would
// make the request right and the output wrong. Note also that :2333 comes
// AFTER print_selected_addrinfo at :2332, so `ip -s addr show` puts the stats
// block below the address lines, where `ip -s link show` puts it directly
// under the link stanza. LinkView.WithStats renders the second layout, so the
// renderer cannot simply be reused.
func addrShowDev(c *runCtx, name string) error {
	svc := service.New(c.src, c.nextSeq)

	// Request one: ll_name_to_index. Nothing is rendered from this reply, and
	// it is byte-identical to `link show dev`'s first request because it is
	// the same iproute2 function.
	resolved, err := svc.LinkByName(name)
	if err != nil {
		return err
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(resolved)})

	// Request two: ipaddr_link_get, by index, carrying preferred_family.
	shown, err := svc.AddrLinkGet(c.family, resolved.Index, req.ExtMaskShow)
	if err != nil {
		return err
	}
	link := xtcpnl.LinkInfo(shown)
	c.lltab.Fill([]xtcpnl.LinkInfo{link})

	// Request three: the address dump, filtered to this index by the kernel.
	// Skipped under AF_PACKET, the same condition and the same reason as in
	// addrShow — ip/ipaddress.c:2310 guards both.
	var addrs []xtcpnl.AddrInfo
	if c.family != unix.AF_PACKET {
		addrResources, aerr := svc.Addresses(c.family, uint32(link.Index))
		if aerr != nil {
			return aerr
		}
		addrs = make([]xtcpnl.AddrInfo, len(addrResources))
		for i := range addrResources {
			addrs[i] = xtcpnl.AddrInfo(addrResources[i])
		}
	}

	resolveLinkRefs(c, svc, link)
	return renderAddrGroups(c, []xtcpnl.LinkInfo{link}, addrs)
}

// renderAddrGroups is the half of `addr show` that is the same whether one
// link arrived from a single-get or all of them arrived from a dump: apply
// ipaddr_filter, group the addresses under their links, and write.
//
// addrs is nil under AF_PACKET, which is not a special case here — the
// per-link inner loop simply runs zero times, and filterLinksWithAddrs is
// skipped because `ip` skips ipaddr_filter for that family too
// (ip/ipaddress.c:2310-2318 guards the dump and the filter together).
func renderAddrGroups(c *runCtx, links []xtcpnl.LinkInfo, addrs []xtcpnl.AddrInfo) error {
	if c.family != unix.AF_PACKET {
		links = filterLinksWithAddrs(links, addrs, c.family)
	}
	if err := checkDetailSupported(c, links); err != nil {
		return err
	}

	// Both of these loops, and the two in filterLinksWithAddrs below, range by
	// index rather than by value. Both resource structs are large, and an
	// AF_UNSPEC `addr show` runs the inner loop len(links) x len(addrs) times —
	// so the value form copies both structs on every one of those iterations to
	// read two integer fields off each.
	groups := make([]render.AddrGroupView, 0, len(links))
	for i := range links {
		// Two different flags on two lines, and they are easy to read as one.
		//
		// LinkViewForAddr's last argument is show_details, which here restores
		// the `link/` line that -4 and -6 suppress (ip/ipaddress.c:1060).
		// WithDetail's is do_link, which is FALSE on this path however many
		// -d's were given: do_link is set by the `ip link show` entry point
		// alone (:2417), and it suppresses exactly one token of the detail
		// run, addrgenmode, via print_af_spec's guard at :1185-1186. The
		// committed pair is the evidence — ip_link_n has addrgenmode on every
		// link and ip_addr_n on none.
		lv := render.LinkViewForAddr(links[i], c.lltab, c.family, c.detailed())
		if c.detailed() {
			lv = lv.WithDetail(links[i], false)
		}
		g := render.AddrGroupView{
			LinkView: lv,
			// Non-nil so the JSON is `"addr_info": []` rather than null for a
			// link with no addresses. iproute2 opens the array
			// unconditionally (open_json_array at the top of
			// print_selected_addrinfo), so an empty array is what `ip -j`
			// emits and null would be a key-shape divergence.
			AddrInfo: []render.AddrView{},
		}
		for j := range addrs {
			if !addrBelongsTo(&addrs[j], &links[i], c.family) {
				continue
			}
			g.AddrInfo = append(g.AddrInfo, render.AddrViewOf(addrs[j]))
		}
		groups = append(groups, g)
	}

	if c.json {
		return json.NewEncoder(c.out).Encode(groups)
	}
	for i := range groups {
		if _, werr := fmt.Fprint(c.out, groups[i].Text()); werr != nil {
			return werr
		}
	}
	return nil
}

// addrBelongsTo is print_selected_addrinfo's per-address test
// (ip/ipaddress.c:1712-1727), minus the `up`/`down` filters goip does not
// implement: the ifindex must match and, when a family was requested, so must
// the family.
//
// The family test is not redundant with the request's ifa_family even though
// the kernel honors that too. `ip` applies it again on the reply, and it has
// to: the same list is walked once per link, and an AF_UNSPEC dump legitimately
// mixes families.
//
// Takes pointers because its caller is the inner loop of a len(links) x
// len(addrs) walk and this reads three integer fields; by value it would copy
// 304 bytes per call to do that.
func addrBelongsTo(ai *xtcpnl.AddrInfo, li *xtcpnl.LinkInfo, family uint8) bool {
	if int32(ai.Index) != li.Index {
		return false
	}
	return family == unix.AF_UNSPEC || family == ai.Family
}

// filterLinksWithAddrs is ipaddr_filter (ip/ipaddress.c:2200-2250): drop every
// link that has no address matching the filter, then put back the links that
// have no addresses *at all* when no family was asked for.
//
// # The two-variable condition is the whole function, and it is easy to get wrong
//
// iproute2 tracks `ok` and `missing_net_address` separately:
//
//	if (missing_net_address &&
//	    (filter.family == AF_UNSPEC || filter.family == AF_PACKET))
//		ok = 1;
//
// `missing_net_address` is cleared by an address with a *matching ifindex*,
// before the family test, so it means "this link has no addresses of any
// family" and not "none that matched". The consequences on the committed
// fixture, all three of them real:
//
//   - nlmon0 has no addresses at all, so plain `addr show` prints it (with no
//     address lines) and `-4 addr show` and `-6 addr show` both drop it.
//   - veth179a698 has one IPv6 address and no IPv4 one, so `-4 addr show`
//     drops it — `missing_net_address` is 0, so the rescue does not apply.
//   - lo has both, so it survives every filter.
//
// Collapsing the two flags into one produces a goip that prints eleven stanzas
// for `-4 addr show` where `ip` prints nine, which is the kind of divergence
// the harness's line-count assertion is for.
func filterLinksWithAddrs(links []xtcpnl.LinkInfo, addrs []xtcpnl.AddrInfo, family uint8) []xtcpnl.LinkInfo {
	out := make([]xtcpnl.LinkInfo, 0, len(links))
	for i := range links {
		ok := false
		missingNetAddress := true
		for j := range addrs {
			if int32(addrs[j].Index) != links[i].Index {
				continue
			}
			missingNetAddress = false
			if family != unix.AF_UNSPEC && family != addrs[j].Family {
				continue
			}
			ok = true
			break
		}
		if missingNetAddress && (family == unix.AF_UNSPEC || family == unix.AF_PACKET) {
			ok = true
		}
		if ok {
			out = append(out, links[i])
		}
	}
	return out
}
